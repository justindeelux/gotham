import{f as g}from"./format-length-C09nWkk6.js";import{$ as v,as as l,_ as d,d as y,a0 as h,d3 as C,a1 as m,a2 as _,a4 as b,au as $,g as a,dm as z}from"./index-Cu5F3ch9.js";var x=v("icon",`
 height: 1em;
 width: 1em;
 line-height: 1em;
 text-align: center;
 display: inline-block;
 position: relative;
 fill: currentColor;
`,[l("color-transition",{transition:"color .3s var(--n-bezier)"}),l("depth",{color:"var(--n-color)"},[d("svg",{opacity:"var(--n-opacity)",transition:"opacity .3s var(--n-bezier)"})]),d("svg",{height:"1em",width:"1em"})]);const R={...h.props,depth:[String,Number],size:[Number,String],color:String,component:[Object,Function]},w=y({_n_icon__:!0,name:"Icon",inheritAttrs:!1,props:R,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:n}=b(e),t=h("Icon","-icon",x,z,e,o),i=a(()=>{const{depth:r}=e,{common:{cubicBezierEaseInOut:c},self:p}=t.value;if(r!==void 0){const{color:u,[`opacity${r}Depth`]:f}=p;return{"--n-bezier":c,"--n-color":u,"--n-opacity":f}}return{"--n-bezier":c,"--n-color":"","--n-opacity":""}}),s=n?$("icon",a(()=>`${e.depth||"d"}`),i,e):void 0;return{mergedClsPrefix:o,mergedStyle:a(()=>{const{size:r,color:c}=e;return{fontSize:g(r),color:c}}),cssVars:n?void 0:i,themeClass:s?.themeClass,onRender:s?.onRender}},render(){const{$parent:e,depth:o,mergedClsPrefix:n,component:t,onRender:i,themeClass:s}=this;return e?.$options?._n_icon__&&C("icon","don't wrap `n-icon` inside `n-icon`"),i?.(),m("i",_(this.$attrs,{role:"img",class:[`${n}-icon`,s,{[`${n}-icon--depth`]:o,[`${n}-icon--color-transition`]:o!==void 0}],style:[this.cssVars,this.mergedStyle]}),t?m(t):this.$slots.default?.())}});export{w as N};
