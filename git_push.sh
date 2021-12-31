param=$1
if [ ! $param ]
then
	echo "please enter some message!"
	exit
fi

git add . 
git commit -m "$param" 
git push origin feature-springboot-leomi
